import {RoomManage} from './RoomManage';
import {useRoom} from './useRoom';
import {Room} from './Room';
import {PasswordPrompt} from './PasswordPrompt';
import {UseConfig, useConfig} from './useConfig';

export const Router = () => {
    const config = useConfig();

    if (config.loading) {
        // show spinner
        return null;
    }
    return <RouterLoadedConfig config={config} />;
};

const RouterLoadedConfig = ({config}: {config: UseConfig}) => {
    const {room, state, passwordRequired, ...other} = useRoom(config);

    if (state) {
        return <Room state={state} {...other} />;
    }

    if (passwordRequired) {
        return (
            <PasswordPrompt
                info={passwordRequired}
                onSubmit={(password) =>
                    room({type: 'join', payload: {id: passwordRequired.id, password}})
                }
            />
        );
    }

    return <RoomManage room={room} config={config} />;
};
