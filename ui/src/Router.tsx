import {RoomManage} from './RoomManage';
import {useRoom} from './useRoom';
import {Room} from './Room';
import {UseConfig, useConfig} from './useConfig';
import {PasswordDialog} from './PasswordDialog';

export const Router = () => {
    const config = useConfig();

    if (config.loading) {
        // show spinner
        return null;
    }
    return <RouterLoadedConfig config={config} />;
};

const RouterLoadedConfig = ({config}: {config: UseConfig}) => {
    const {room, state, passwordPrompt, submitPassword, cancelPassword, ...other} =
        useRoom(config);

    return (
        <>
            {state ? (
                <Room state={state} {...other} />
            ) : (
                <RoomManage room={room} config={config} />
            )}
            <PasswordDialog
                open={passwordPrompt.open}
                roomId={passwordPrompt.roomId}
                error={passwordPrompt.error}
                onSubmit={submitPassword}
                onCancel={cancelPassword}
            />
        </>
    );
};
